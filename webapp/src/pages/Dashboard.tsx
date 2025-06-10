import { Box, VStack, Alert, AlertIcon, AlertTitle, AlertDescription, Tabs, TabList, TabPanels, Tab, TabPanel } from '@chakra-ui/react'
import { ApiUrlConfig } from '~components/ApiUrlConfig'
import { ContractLinks } from '~components/ContractLinks'
import { ProcessList } from '~components/ProcessList'
import { Workers } from '~components/Workers'
import { useSequencerInfo } from '~hooks/useSequencerAPI'

const Dashboard = () => {
  const { data: info, error: infoError, isLoading: infoLoading } = useSequencerInfo()

  return (
    <VStack spacing={8} align="stretch">
      {/* API URL Configuration */}
      <Box>
        <ApiUrlConfig />
      </Box>

      {/* Contract Links Section */}
      <Box>
        {infoError && (
          <Alert status="error" mb={4}>
            <AlertIcon />
            <AlertTitle>Error loading sequencer info</AlertTitle>
            <AlertDescription>{infoError.message}</AlertDescription>
          </Alert>
        )}
        <ContractLinks contracts={info?.contracts} isLoading={infoLoading} />
      </Box>

      {/* Tabbed Content Section */}
      <Box>
        <Tabs colorScheme="purple" variant="enclosed">
          <TabList>
            <Tab>Active Processes</Tab>
            <Tab>Workers</Tab>
          </TabList>
          <TabPanels>
            <TabPanel px={0}>
              <ProcessList />
            </TabPanel>
            <TabPanel px={0}>
              <Workers />
            </TabPanel>
          </TabPanels>
        </Tabs>
      </Box>
    </VStack>
  )
}

export default Dashboard
